<?xml version="1.0" encoding="UTF-8"?>
<!-- Renders an invoice, a value of /TRFIN/INVOICE, as HTML in English or German. -->
<xsl:stylesheet version="3.0" xmlns:xsl="http://www.w3.org/1999/XSL/Transform"
    xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" exclude-result-prefixes="xsi">
  <xsl:param name="LANG" select="'en'"/>
  <xsl:output method="html" html-version="5" indent="no"/>
  <xsl:template match="/value">
    <html lang="{$LANG}">
      <body>
        <h1><xsl:value-of select="if ($LANG = 'de') then 'Rechnung' else 'Invoice'"/><xsl:text> </xsl:text><xsl:value-of select="INVOICE_ID"/></h1>
        <p class="date"><xsl:value-of select="ISSUED_ON"/></p>
        <xsl:if test="not(NOTE/@xsi:nil = 'true')">
          <p class="note"><xsl:value-of select="NOTE"/></p>
        </xsl:if>
        <table>
          <xsl:for-each select="ITEMS/item">
            <tr><td><xsl:value-of select="POSITION"/></td><td><xsl:value-of select="TEXT"/></td><td><xsl:value-of select="AMOUNT"/></td></tr>
          </xsl:for-each>
        </table>
        <p class="total"><xsl:value-of select="if ($LANG = 'de') then 'Summe' else 'Total'"/>: <xsl:value-of select="TOTAL"/></p>
      </body>
    </html>
  </xsl:template>
</xsl:stylesheet>
